var contactForm = (function () {
	$("#contact-form").submit(function() {
		$.ajax({
			type: 'POST',
			url: cAPIBase + '/contact',
			data: {
				mail: $("#contact-email").val(),
				content: $("#contact-content").val()
			},
			success: function() {
				var contactButton = $("#contact-submit");
				contactButton.text("Sent!");
				contactButton.removeClass("btn-success");
				contactButton.addClass("btn-primary");
			},
		});
		event.preventDefault();
	});
})();
